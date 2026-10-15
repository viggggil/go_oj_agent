n=int(input()); a=list(map(int,input().split())); p=q=0
for x in a: p,q=q,max(q,p+x)
print(q)
