from bisect import bisect_left
n=int(input()); a=list(map(int,input().split())); tails=[]
for x in a:
    k=bisect_left(tails,x)
    if k==len(tails): tails.append(x)
    else: tails[k]=x
print(len(tails))
