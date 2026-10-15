n,k=map(int,input().split()); a=list(map(int,input().split())); count={0:1}; s=ans=0
for x in a:
    s+=x; ans+=count.get(s-k,0); count[s]=count.get(s,0)+1
print(ans)
